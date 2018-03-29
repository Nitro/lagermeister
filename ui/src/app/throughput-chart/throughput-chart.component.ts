import { Component } from '@angular/core';
import * as _ from "lodash";

// Services
import { ChartsService } from '../charts.service';

@Component({
    selector: 'throughput-chart',
    templateUrl: './throughput-chart.component.html'
})

export class ThroughtputChartComponent {

    throughputChartObject: any;
    chartConfig: any;
    pendingThroughput: Array<any>;

    constructor(private chartsService: ChartsService) {

        this.chartsService.fetchConfig()
            .then( (resp: any) => {
                this.chartConfig = resp;

                let columns = [['Timestamp', 'datetime']];
                _.each(_.keys(this.chartConfig.Throughput).sort().reverse(), (key: string) => {
                            columns.push([key, 'number']);
                        });

                this.throughputChartObject = {
                    chartType: 'LineChart',
                    data: {},
                    chart: null,
                    domElement: 'throughput-chart',
                    columns: columns,
                    options: Object.assign({
                        vAxis: {
                            title: 'Throughput (rps)',
                        },
                        hAxis: {
                            viewWindow: {}
                        },
                        colors: ['#FFC000', '#208DB9'],
                    }, this.chartsService.getLineChartOptions())
                };

                this.setupChart();
            });
    }

    setupChart() {
        (<any>window).google.charts.setOnLoadCallback(this.chartsService.drawLineChart.bind(this, this.throughputChartObject));

        this.pendingThroughput = [];

        this.chartsService.getObservable().subscribe( (message: any) => {
            let data = JSON.parse(message.data);
            if (data.MetricType === 'Throughput') {
                this.aggregateData(data);
            }
        });

        // On a 1 second basis, we process everything that was aggregated and pass it to the
        // chart for drawing. But we hold back the last value and anything in the 2 seconds
        // before, so that we can still aggregate any slow arrivals into those two second buckets.
        setInterval(() => {
            let lastTimestamp = this.pendingThroughput[this.pendingThroughput.length-1].Timestamp;
            let leaveBehindCount = 0; // Number of values to leave in the array after processing
            _.forEach(this.pendingThroughput, (data:any,) => {
                if (data.Timestamp < lastTimestamp-2) {
                    if (this.throughputChartObject.chartReady) {
                        this.chartsService.updateLineChart(data, this.throughputChartObject);
                    }
                } else {
                    leaveBehindCount += 1;
                }
            });

            this.pendingThroughput = this.pendingThroughput.slice(this.pendingThroughput.length - leaveBehindCount, this.pendingThroughput.length);
        }, 1000)
    }

    /*
     * Function used to build the proper throughput metrics
     * And push the data to the pending array
     */
    aggregateData(data: any) {

        this.pendingThroughput.push(data);

        let grouped = _.groupBy(this.pendingThroughput, (evt:any) => {
            return evt.Timestamp;
        });

        let combined:Array<any> = [];
        _.forEach(grouped, (items:any, time:any) => {
            let combinedItem = {
                Values: {},
                MetricType: '',
                Timestamp: 0,
                Sender: ''
            };

            _.forEach(items, (item:any) => {
                Object.assign(combinedItem, item);
                combinedItem.Sender = item.Sender;
                combinedItem.MetricType = item.MetricType;
                combinedItem.Timestamp = item.Timestamp;
                combinedItem.Values[item.Sender] = item.Value;
            });

            // Make sure we have all the columns defined or the chart goes ape
            _.forEach(this.chartConfig.Throughput, (val:any, key:any) => {
                combinedItem.Values[key] = combinedItem.Values[key] ? combinedItem.Values[key] : 0;
            });

            combined.push(combinedItem);
        });
        this.pendingThroughput = combined;
    }
}

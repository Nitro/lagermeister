import { Component } from '@angular/core';
import * as _ from "lodash";

// Services
import { ChartsService } from '../charts.service';

@Component({
    selector: 'batch-count-chart',
    templateUrl: './batch-count-chart.component.html',
    providers: [ChartsService],
})

export class BatchCountChartComponent {

    batchCountChartObject: any;

    constructor(private chartsService: ChartsService) {

        this.batchCountChartObject = {
            chartType: 'LineChart',
            data: {},
            chart: null,
            domElement: 'batch-count-chart',
            columns: [['Timestamp', 'datetime'], ['Batch Count', 'number']],
            options: Object.assign({
                width: '100%',
                vAxis: {
                    title: 'BatchCount',
                },
                colors: ['#4CAF50'],
            }, this.chartsService.getLineChartOptions())
        };

        let script = document.createElement('script');
        script.src = '//www.google.com/jsapi';
        script.onload = () => {
            (<any>window).google.charts.load("visualization", "1", {packages:["corechart", "gauge"]});
            (<any>window).google.charts.setOnLoadCallback(this.chartsService.drawLineChart.bind(this, this.batchCountChartObject));
        };
        document.head.appendChild(script);

        let pendingData: Array<Object> = [];

        let obsv = this.chartsService.getObservable();
        obsv.subscribe( (evt: any)  => {
            let data = JSON.parse(evt.data);
            if (data.MetricType === 'BatchCount') {
                pendingData.push(data);
            }
        });

        setInterval(() => {
            _.forEach(pendingData, (data: any) => {
                if(this.batchCountChartObject.chartReady) {
                    this.chartsService.updateLineChart(data, this.batchCountChartObject);
                }
            });
            pendingData.splice(0);
        }, 1000)

    }
}

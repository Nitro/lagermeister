import { Component } from '@angular/core';
import * as _ from "lodash";

// Services
import { ChartsService } from '../charts.service';

@Component({
    selector: 'batch-count-chart',
    templateUrl: './batch-count-chart.component.html'
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

        (<any>window).google.charts.setOnLoadCallback(this.chartsService.drawLineChart.bind(this, this.batchCountChartObject));

        let pendingData: Array<Object> = [];

        this.chartsService.getObservable().subscribe( (message: any) => {
            let data = JSON.parse(message.data);
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
